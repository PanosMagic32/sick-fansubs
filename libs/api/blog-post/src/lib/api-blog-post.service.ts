import { Injectable } from '@nestjs/common';
import { InjectModel } from '@nestjs/mongoose';
import { Model } from 'mongoose';

import { CreateBlogPostDto } from './dtos/create-blog-post.dto';
import { BlogPost, BlogPostDocument } from './schemas/blog-post.schema';

@Injectable()
export class ApiBlogPostService {
  constructor(@InjectModel(BlogPost.name) private readonly blogPostModel: Model<BlogPostDocument>) {}

  async create(createBlogPostDto: CreateBlogPostDto): Promise<BlogPost> {
    const createdCat = await this.blogPostModel.create(createBlogPostDto);
    return createdCat;
  }

  async findAll(): Promise<BlogPost[]> {
    return this.blogPostModel.find().exec();
  }

  async findOne(id: string): Promise<BlogPost | undefined> {
    const blogPost = await this.blogPostModel.findOne({ _id: id });

    if (blogPost) {
      return blogPost;
    } else {
      return undefined;
    }
  }

  async delete(id: string) {
    const deletedCat = await this.blogPostModel.findByIdAndRemove({ _id: id }).exec();
    return deletedCat;
  }
}