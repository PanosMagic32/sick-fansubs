import { Prop, Schema, SchemaFactory } from '@nestjs/mongoose';
import { Document } from 'mongoose';

export type BlogPostDocument = BlogPost & Document;

@Schema()
export class BlogPost {
  @Prop({ required: true })
  title!: string;

  @Prop({ required: true })
  description!: string;
}

export const BlogPostSchema = SchemaFactory.createForClass(BlogPost);
