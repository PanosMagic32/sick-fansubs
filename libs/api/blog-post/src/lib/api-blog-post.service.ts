import { Injectable, NotFoundException } from '@nestjs/common';
import { InjectModel } from '@nestjs/mongoose';
import { FilterQuery, Model } from 'mongoose';

import { CreateBlogPostDto } from './dtos/create-blog-post.dto';
import { UpdateBlogPostDto } from './dtos/update-blog-post.dto';
import { BlogPost, BlogPostDocument } from './schemas/blog-post.schema';

@Injectable()
export class ApiBlogPostService {
  constructor(@InjectModel(BlogPost.name) private readonly blogPostModel: Model<BlogPostDocument>) {}

  async create(createBlogPostDto: CreateBlogPostDto): Promise<{ id: string }> {
    const createdBlogPost = await this.blogPostModel.create(createBlogPostDto);
    return { id: createdBlogPost._id as string };
  }

  async findAll(
    pageSize: number,
    currentPage: number,
    // startId?: string
  ): Promise<{ posts: BlogPost[]; count: number }> {
    const query = this.blogPostModel
      // The below commented-out object in find is a possible way to improve performance in database search
      // .find({
      //   _id: {
      //     $gt: startId,
      //   },
      // })
      .find()
      .sort({ dateTimeCreated: 'desc' });

    if (pageSize && currentPage) {
      query.skip(pageSize * currentPage).limit(pageSize);
    }

    const posts = await query.exec();
    const count = await this.blogPostModel.countDocuments();

    return { posts, count };
  }

  async findOne(id: string): Promise<BlogPost | undefined> {
    const blogPost = await this.blogPostModel.findOne({ _id: id });
    if (blogPost) return blogPost;
    throw new NotFoundException();
  }

  async update(id: string, updateBlogPostDto: UpdateBlogPostDto): Promise<BlogPost | undefined | null> {
    const blogPost = await this.findOne(id);
    if (blogPost) return this.blogPostModel.findByIdAndUpdate({ _id: id }, updateBlogPostDto).exec();
    throw new NotFoundException();
  }

  async delete(id: string) {
    const deletedBlogPost = await this.blogPostModel.findByIdAndDelete({ _id: id }).exec();
    return deletedBlogPost;
  }

  async count(options: FilterQuery<BlogPostDocument>) {
    return this.blogPostModel.countDocuments(options).sort({ dateTimeCreated: 'desc' }).exec();
  }
}
