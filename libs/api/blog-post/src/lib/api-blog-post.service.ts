import { Injectable, NotFoundException } from '@nestjs/common';
import { InjectModel } from '@nestjs/mongoose';
import { FilterQuery, Model, Types } from 'mongoose';

import { MediaService } from '@api/media';
import { UserService } from '@api/user';

import { CreateBlogPostDto } from './dtos/create-blog-post.dto';
import { UpdateBlogPostDto } from './dtos/update-blog-post.dto';
import { BlogPost, BlogPostDocument } from './schemas/blog-post.schema';

@Injectable()
export class ApiBlogPostService {
  constructor(
    @InjectModel(BlogPost.name) private readonly blogPostModel: Model<BlogPostDocument>,
    private readonly mediaService: MediaService,
    private readonly userService: UserService,
  ) {}

  private sanitizeEditedMetadata<T extends { updatedBy?: unknown; updatedAt?: Date | string }>(entity: T): T {
    if (!entity.updatedBy) {
      entity.updatedAt = undefined;
    }

    return entity;
  }

  async create(createBlogPostDto: CreateBlogPostDto, creatorId: string): Promise<{ id: string }> {
    const createdBlogPost = await this.blogPostModel.create({
      ...createBlogPostDto,
      creator: new Types.ObjectId(creatorId),
    });

    await this.userService.addCreatedBlogPost(creatorId, createdBlogPost._id.toString());

    return { id: createdBlogPost._id.toString() };
  }

  async findAll(
    pageSize: number,
    currentPage: number,
    // startId?: string
  ): Promise<{ posts: BlogPost[]; count: number }> {
    const query = this.blogPostModel
      .find()
      .populate('creator', 'username avatar')
      .populate('updatedBy', 'username avatar')
      .sort({ dateTimeCreated: 'desc' });

    if (pageSize && currentPage) {
      query.skip(pageSize * currentPage).limit(pageSize);
    }

    const posts = await query.exec();
    const count = await this.blogPostModel.countDocuments();

    return { posts: posts.map((post) => this.sanitizeEditedMetadata(post)), count };
  }

  async findOne(id: string): Promise<BlogPost | undefined> {
    const blogPost = await this.blogPostModel
      .findOne({ _id: id })
      .populate('creator', 'username avatar')
      .populate('updatedBy', 'username avatar');
    if (blogPost) return this.sanitizeEditedMetadata(blogPost);
    throw new NotFoundException();
  }

  async update(id: string, updateBlogPostDto: UpdateBlogPostDto, actorId: string): Promise<BlogPost | undefined | null> {
    const existingBlogPost = await this.blogPostModel.findById(id).select('thumbnail').exec();
    if (existingBlogPost) {
      const updatedBlogPost = await this.blogPostModel
        .findByIdAndUpdate(
          { _id: id },
          { ...updateBlogPostDto, updatedBy: new Types.ObjectId(actorId) },
          { new: true, runValidators: true },
        )
        .exec();

      if (updatedBlogPost && existingBlogPost.thumbnail !== updatedBlogPost.thumbnail) {
        await this.mediaService.deleteManagedImageByUrl(existingBlogPost.thumbnail);
      }

      return updatedBlogPost;
    }

    throw new NotFoundException();
  }

  async delete(id: string, actorId: string) {
    const existingBlogPost = await this.blogPostModel.findById(id).exec();
    if (!existingBlogPost) {
      throw new NotFoundException();
    }

    await this.blogPostModel.deleteOne({ _id: id }).exec();

    const creatorId = existingBlogPost.creator instanceof Types.ObjectId ? existingBlogPost.creator.toString() : actorId;

    await this.userService.removeCreatedBlogPost(creatorId, id);
    await this.mediaService.deleteManagedImageByUrl(existingBlogPost.thumbnail);

    return existingBlogPost;
  }

  async count(options: FilterQuery<BlogPostDocument>) {
    return this.blogPostModel.countDocuments(options).sort({ dateTimeCreated: 'desc' }).exec();
  }
}
